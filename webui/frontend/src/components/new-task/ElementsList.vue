<template>
  <v-card outlined>

    <v-card-title>
      {{ cardTitle }}
    </v-card-title>

    <v-card-subtitle v-if="cardSubtitle !== ''" align='left'>
      {{ cardSubtitle }}
    </v-card-subtitle>

    <v-card-text>

      <draggable v-model='userElementsComputed' :disabled='dragAndDrop'>
        <div
          v-for="(userElement, index) in userElementsComputed"
          :key="userElement.ID + '_' + $uuid.v1()"
          class="my-2"
        >
          <v-row v-if='userElement.order !== 0 && cardTitle === "Actions"' class="my-1" justify='center'>
            <v-icon color='blue darken-2' large>mdi-arrow-down-bold</v-icon>
            <v-icon v-if="userElement.chained" color='blue darken-2'>mdi-link-variant</v-icon>
          </v-row>
          <v-card :elevation='4'>
            <v-card-title>
              {{ userElement.name }}
            </v-card-title>
            <v-card-subtitle>
              {{ userElement.description }}
            </v-card-subtitle>
            <v-card-text>
              <v-expansion-panels>
                <v-expansion-panel v-for="arg in userElement.args" :key="arg.ID">
                  <v-expansion-panel-header
                    :disable-icon-rotate='userElement.argumentToReplaceByCR === arg.ID'
                  >
                    {{ arg.name }}
                    <template
                      v-if='userElement.argumentToReplaceByCR === arg.ID'
                      v-slot:actions
                    >
                      <v-icon color="blue darken-2">mdi-link-box-variant</v-icon>
                    </template>
                  </v-expansion-panel-header>
                  <v-expansion-panel-content class="text--secondary text--darken-2">
                    {{ arg.description }}
                    <app-adaptative-arg
                      :content='arg.content'
                      :argType="arg.contentType"
                      :disabled='userElement.argumentToReplaceByCR === arg.ID && userElement.chained'
                      @changed='arg.content = $event'
                    />
                    <div
                      v-if='userElement.order !== 0 && cardTitle === "Actions"'
                      class="d-flex flex-row-reverse"
                    >
                      <v-btn
                        v-if="userElement.argumentToReplaceByCR !== arg.ID"
                        @click='userElement.argumentToReplaceByCR = arg.ID; userElement.chained = true'
                        text
                        icon
                      >
                        <v-icon>mdi-link-variant</v-icon>
                      </v-btn>
                      <v-btn
                        v-else
                        color='red lighten-1'
                        @click='userElement.argumentToReplaceByCR = ""; userElement.chained = false'
                        text
                        icon
                      >
                        <v-icon>mdi-link-variant-off</v-icon>
                      </v-btn>
                    </div>
                  </v-expansion-panel-content>
                </v-expansion-panel>
              </v-expansion-panels>
            </v-card-text>
            <v-card-actions>
              <v-spacer/>
              <v-btn
                color='red lighten-1'
                @click="removeItem(index)"
                text
                icon
              >
                <v-icon>mdi-delete</v-icon>
              </v-btn>
            </v-card-actions>
          </v-card>
        </div>
      </draggable>

      <v-row justify='center'>
        <v-btn color='green' @click="openSelector" text icon>
          <v-icon>mdi-plus</v-icon>
        </v-btn>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script>
import draggable from 'vuedraggable'
import AdaptativeArgSelector from './AdaptativeArgSelector.vue'

export default {
  props: {
    cardTitle: {
      type: String,
      required: true
    },
    cardSubtitle: {
      type: String,
      required: false,
      default: ''
    },
    userElements: {
      type: Array,
      required: true
    },
    dragAndDrop: {
      type: Boolean,
      required: false,
      default: true
    }
  },
  data () {
    return {
    }
  },
  computed: {
    userElementsComputed: {
      get () {
        return this.userElements
      },
      set (newValue) {
        this.$emit('modified', newValue)
      }
    }
  },
  methods: {
    openSelector () {
      this.$emit('open-selector')
    },
    removeItem (elementIndex) {
      this.$emit('remove-item', elementIndex)
    },
    orderUpdateRequired () {
      this.$emit('order-modified')
    }
  },
  watch: {
    userElementsComputed: function (newValue) {
      this.orderUpdateRequired()
    }
  },
  components: {
    draggable,
    appAdaptativeArg: AdaptativeArgSelector
  }
}
</script>
