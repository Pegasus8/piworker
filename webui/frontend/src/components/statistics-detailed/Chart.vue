<script>
import { Line } from 'vue-chartjs'

export default {
  extends: Line,
  props: {
    chartData: {
      required: true,
      type: Object
    },
    options: {
      required: true,
      type: Object
    }
  },
  watch: {
    options () {
      this.renderChart(this.chartData, this.options)
    },
    chartData () {
      this.renderChart(this.chartData, this.options)
    }
  },
  mounted () {
    this.renderChart(this.chartData, this.options)
  }
}
</script>
